import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'
import './styles/base.css'
import { AgentPage } from './pages/AgentPage'
import { AboutPage } from './pages/AboutPage'
import { ChangelogPage } from './pages/ChangelogPage'
import { FavoritesPage } from './pages/FavoritesPage'
import { HotPage } from './pages/HotPage'
import { ItemPage } from './pages/ItemPage'
import { ListPage } from './pages/ListPage'
import { MorePage } from './pages/MorePage'
import { ReportPage } from './pages/ReportPage'
import { ReportsPage } from './pages/ReportsPage'
import { StoryPage } from './pages/StoryPage'
import { TopicPage, TopicsPage } from './pages/TopicsPage'
import { HomeRail } from './ui/HomeRail'
import { Columns, Shell } from './shell/Shell'
import { ErrorPage } from './ui/States'

function App() {
  return <Routes>
    <Route element={<Shell />}>
      <Route path="/" element={<ListPage view="selected" />} />
      <Route path="/all" element={<ListPage view="all" />} />
      <Route path="/favorites" element={<Columns rail={<HomeRail />}><FavoritesPage /></Columns>} />
      <Route path="/hot" element={<HotPage />} />
      <Route path="/topics" element={<TopicsPage />} />
      <Route path="/topics/:slug" element={<TopicPage />} />
      <Route path="/reports" element={<Columns rail={<HomeRail report={false} />}><ReportsPage /></Columns>} />
      <Route path="/reports/:date" element={<ReportPage />} />
      <Route path="/items/:id" element={<Columns rail={<HomeRail />}><ItemPage /></Columns>} />
      <Route path="/stories/:id" element={<Columns rail={<HomeRail />}><StoryPage /></Columns>} />
      <Route path="/more" element={<MorePage />} />
      <Route path="/agent" element={<AgentPage />} />
      <Route path="/about" element={<AboutPage />} />
      <Route path="/changelog" element={<ChangelogPage />} />
      <Route path="*" element={<ErrorPage kind="notfound" what="页面" />} />
    </Route>
  </Routes>
}

createRoot(document.getElementById('root')!).render(<StrictMode><BrowserRouter><App /></BrowserRouter></StrictMode>)
